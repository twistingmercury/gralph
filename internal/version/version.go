package version

import (
	"fmt"
)

const Mascot = `
                                                                                                   
                                                                                                   
                    ###################                                                            
               ###### #  # ##  # #   # ####                                                        
            #### #  #  #  #  ##  #  #  #  #####                                                    
          # #  #  #   #  #   #   #  #   #  # # ##                                                  
        ## #  #  #   #  #   ##   #   #   #  # ## ###                                               
       #  #  #  ##  #   #   ##       #   ##  # ##  ###                                             
      #  #  #  ##  #   #      #                    ## #                                            
     #  #  #   #   #      ###   ###       ######### #                                              
     #  # #    #         #         #     ##        ###                                             
    #  #  #   #          #  ##     #     #    ###  # #                                             
       ###               #  ##     #     #         ###                                             
     #                    #       ##       #     ## ##                                             
     # ###                  ######        ## ###                                                   
     ##                             #      #        ##                                             
       #####                     ##   #   ##          ##                                           
          #                    ##    ## ##             ##                                          
           #              #   #     #                  #                                           
            ##            ## #     ##   ##        ####                                             
           ## ##        ##   #        ############                                                 
           #    ####                  #    #   #                                                   
          ##        ##########             # ##                                                    
         ####             #   ###       ### #   ##                                                 
      ##     ##          #        ##    ##   ###  #                                                
      #         ###    ##           #   #    # ##  #                                               
      #            #####             # ##  ##    ####                                              
      #            ##                 ## ##    #   # #                                             
      ##                           #   #        #     #                                            
     #  #                        ##  ##  #       #     ##                                          
    #   ###                    ##   #   ##        #     ##                                         
   ##     ##                ##    ##  ##           #    ###                                        
   #        ##    ## ######    ###  ##             #     ##                                        
   #          ####         ####                     #    # #                                       
   ##             ##########                        #     ##                                       
  #   ####                                          #     # #                                      
  #        ########                                    #### #                                      
                                                             #                                     
`

var (
	version   = "dev"     // set via -ldflags: -X github.com/twistingmercury/gralph/internal/version.version=<value>
	buildDate = "unknown" // set via -ldflags: -X github.com/twistingmercury/gralph/internal/version.buildDate=<value>
	gitCommit = "unknown" // set via -ldflags: -X github.com/twistingmercury/gralph/internal/version.gitCommit=<value>
)

func Print() {
	fmt.Print(Mascot)
	fmt.Printf("version: %s\ndate: %s\ncommit: %s\n", version, buildDate, gitCommit)
}
